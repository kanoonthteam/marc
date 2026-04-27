---
name: python-testing
description: Python testing mastery — pytest, unittest.mock, fixtures, Flask testing, database integration tests, moto for AWS, factory_boy, pytest-asyncio, coverage, and CI integration
---

# Python Testing Mastery

Production-ready testing patterns for Python 3.11+ backend applications. Covers pytest fundamentals, advanced fixtures, unittest.mock patterns, Flask application testing, database integration testing with transactions, moto for AWS service mocking, subprocess testing, factory_boy for test data, async testing, performance benchmarks, coverage reporting, and CI pipeline integration.

## Table of Contents

1. [Pytest Fundamentals](#pytest-fundamentals)
2. [Test Discovery and Naming Conventions](#test-discovery-and-naming-conventions)
3. [Assertions and Introspection](#assertions-and-introspection)
4. [Marks and Parametrize](#marks-and-parametrize)
5. [Fixtures](#fixtures)
6. [Factory Fixtures and factory_boy](#factory-fixtures-and-factory_boy)
7. [conftest.py Organization](#conftestpy-organization)
8. [unittest.mock — Mock, MagicMock, patch](#unittestmock--mock-magicmock-patch)
9. [Testing Flask Applications](#testing-flask-applications)
10. [Integration Testing with Databases](#integration-testing-with-databases)
11. [Testing boto3/AWS with moto](#testing-boto3aws-with-moto)
12. [Testing Subprocess Calls](#testing-subprocess-calls)
13. [Testing Async Code with pytest-asyncio](#testing-async-code-with-pytest-asyncio)
14. [Performance Testing with pytest-benchmark](#performance-testing-with-pytest-benchmark)
15. [pytest-cov for Coverage Reports](#pytest-cov-for-coverage-reports)
16. [Test Organization and Project Structure](#test-organization-and-project-structure)
17. [CI Integration and Parallel Test Execution](#ci-integration-and-parallel-test-execution)
18. [Best Practices](#best-practices)
19. [Anti-Patterns](#anti-patterns)
20. [Sources & References](#sources--references)

---

## Pytest Fundamentals

Pytest is the de facto testing framework for Python. It provides simple assertion syntax, powerful fixture support, and a rich plugin ecosystem.

### Installation

```
pip install pytest pytest-cov pytest-asyncio pytest-benchmark pytest-xdist factory-boy moto[all] flask
```

Or with a `pyproject.toml`:

```toml
[project.optional-dependencies]
test = [
    "pytest>=8.0",
    "pytest-cov>=5.0",
    "pytest-asyncio>=0.24",
    "pytest-benchmark>=4.0",
    "pytest-xdist>=3.5",
    "factory-boy>=3.3",
    "moto[all]>=5.0",
]
```

### Basic Configuration in pyproject.toml

```toml
[tool.pytest.ini_options]
testpaths = ["tests"]
python_files = ["test_*.py", "*_test.py"]
python_classes = ["Test*"]
python_functions = ["test_*"]
addopts = "-ra -q --strict-markers --tb=short"
markers = [
    "slow: marks tests as slow (deselect with '-m \"not slow\"')",
    "integration: marks tests requiring external services",
    "unit: marks pure unit tests",
]
filterwarnings = [
    "error",
    "ignore::DeprecationWarning:third_party_lib.*",
]
```

---

## Test Discovery and Naming Conventions

Pytest discovers tests automatically based on naming conventions:

- **Files**: must start with `test_` or end with `_test.py`
- **Classes**: must start with `Test` and have no `__init__` method
- **Functions**: must start with `test_`

### Recommended Directory Layout

```
project/
├── src/
│   └── myapp/
│       ├── __init__.py
│       ├── models.py
│       ├── services.py
│       └── api/
│           ├── __init__.py
│           └── routes.py
├── tests/
│   ├── __init__.py
│   ├── conftest.py
│   ├── unit/
│   │   ├── __init__.py
│   │   ├── conftest.py
│   │   ├── test_models.py
│   │   └── test_services.py
│   ├── integration/
│   │   ├── __init__.py
│   │   ├── conftest.py
│   │   └── test_api_routes.py
│   └── e2e/
│       ├── __init__.py
│       └── test_workflows.py
├── pyproject.toml
└── Makefile
```

### Naming Best Practices

- Name test functions descriptively: `test_create_user_with_duplicate_email_raises_conflict`
- Group related tests in classes: `TestUserCreation`, `TestUserDeletion`
- Use the pattern `test_<unit>_<scenario>_<expected_result>`

---

## Assertions and Introspection

Pytest rewrites `assert` statements to provide rich introspection on failure. No special assertion methods are needed.

```python
# tests/unit/test_assertions.py
from __future__ import annotations

import pytest

from myapp.models import User, Order, OrderStatus


class TestAssertionPatterns:
    """Demonstrate pytest assertion introspection."""

    def test_equality(self) -> None:
        user = User(name="Alice", email="alice@example.com")
        assert user.name == "Alice"
        assert user.email == "alice@example.com"

    def test_collection_membership(self) -> None:
        roles = ["admin", "editor", "viewer"]
        assert "admin" in roles
        assert "superuser" not in roles

    def test_approximate_equality(self) -> None:
        """Use pytest.approx for floating point comparisons."""
        calculated_tax = 19.999999999
        assert calculated_tax == pytest.approx(20.0, rel=1e-6)

    def test_exception_raised(self) -> None:
        """Verify specific exceptions with match patterns."""
        with pytest.raises(ValueError, match=r"Email .* is already registered"):
            User.register(name="Bob", email="existing@example.com")

    def test_exception_attributes(self) -> None:
        """Inspect the raised exception object."""
        with pytest.raises(Order.InvalidTransitionError) as exc_info:
            order = Order(status=OrderStatus.SHIPPED)
            order.transition_to(OrderStatus.PENDING)

        assert exc_info.value.from_status == OrderStatus.SHIPPED
        assert exc_info.value.to_status == OrderStatus.PENDING

    def test_warnings(self) -> None:
        """Assert that specific warnings are emitted."""
        with pytest.warns(DeprecationWarning, match="use create_order_v2"):
            Order.create_order_v1(item="widget", quantity=1)

    def test_dictionary_subset(self) -> None:
        """Check that a dict contains expected keys."""
        response = {"id": 42, "name": "Alice", "role": "admin", "created_at": "2026-01-01"}
        assert response["name"] == "Alice"
        assert response["role"] == "admin"
        # Check subset
        expected = {"name": "Alice", "role": "admin"}
        assert expected.items() <= response.items()
```

---

## Marks and Parametrize

Marks let you categorize tests and control execution. `parametrize` generates multiple test cases from a single function.

### Built-in Marks

- `@pytest.mark.skip(reason="...")` — unconditionally skip
- `@pytest.mark.skipif(condition, reason="...")` — conditional skip
- `@pytest.mark.xfail(reason="...")` — expect failure
- `@pytest.mark.parametrize(...)` — parametrized test cases

### Custom Marks

Register custom marks in `pyproject.toml` (shown above) and apply them:

```python
# tests/unit/test_marks.py
from __future__ import annotations

import sys
from typing import Any

import pytest

from myapp.services import calculate_discount, validate_email, parse_csv_row


@pytest.mark.slow
def test_large_dataset_processing() -> None:
    """This test is slow; run only when needed."""
    results = process_million_records()
    assert len(results) == 1_000_000


@pytest.mark.skipif(sys.platform == "win32", reason="Unix-only feature")
def test_unix_socket_connection() -> None:
    """Tests Unix domain socket connectivity."""
    ...


@pytest.mark.parametrize(
    ("input_price", "discount_pct", "expected"),
    [
        (100.0, 10, 90.0),
        (100.0, 0, 100.0),
        (100.0, 100, 0.0),
        (49.99, 25, 37.4925),
        (0.0, 50, 0.0),
    ],
    ids=[
        "ten_percent_off",
        "no_discount",
        "full_discount",
        "fractional_price",
        "zero_price",
    ],
)
def test_calculate_discount(input_price: float, discount_pct: int, expected: float) -> None:
    result = calculate_discount(price=input_price, discount_percent=discount_pct)
    assert result == pytest.approx(expected)


@pytest.mark.parametrize(
    ("email", "is_valid"),
    [
        ("user@example.com", True),
        ("user+tag@example.com", True),
        ("user@.com", False),
        ("@example.com", False),
        ("user@example", False),
        ("", False),
    ],
)
def test_validate_email(email: str, is_valid: bool) -> None:
    assert validate_email(email) is is_valid


class TestCSVParsing:
    """Group parametrized tests in a class for organization."""

    @pytest.mark.parametrize(
        ("row", "expected"),
        [
            ("Alice,30,admin", {"name": "Alice", "age": 30, "role": "admin"}),
            ("Bob,25,editor", {"name": "Bob", "age": 25, "role": "editor"}),
        ],
    )
    def test_parse_csv_row(self, row: str, expected: dict[str, Any]) -> None:
        parsed = parse_csv_row(row, headers=["name", "age", "role"])
        assert parsed == expected
```

Run filtered tests:

```
pytest -m "not slow"           # Skip slow tests
pytest -m "unit"               # Only unit tests
pytest -m "integration"        # Only integration tests
pytest -k "test_validate"      # Keyword-based filtering
```

---

## Fixtures

Fixtures provide setup/teardown logic, dependency injection, and test isolation. They are the core of pytest's power.

### Fixture Scopes

- `function` (default): runs once per test function
- `class`: runs once per test class
- `module`: runs once per test module
- `package`: runs once per test package
- `session`: runs once per test session

### Basic Fixtures

```python
# tests/conftest.py
from __future__ import annotations

from collections.abc import Generator
from typing import Any

import pytest

from myapp.config import TestConfig
from myapp.database import Database, Session
from myapp.models import User


@pytest.fixture(scope="session")
def db_engine() -> Generator[Any, None, None]:
    """Create a database engine for the entire test session."""
    engine = Database.create_engine(TestConfig.DATABASE_URL)
    Database.create_all_tables(engine)
    yield engine
    Database.drop_all_tables(engine)
    engine.dispose()


@pytest.fixture(scope="function")
def db_session(db_engine: Any) -> Generator[Session, None, None]:
    """Provide a transactional database session that rolls back after each test."""
    connection = db_engine.connect()
    transaction = connection.begin()
    session = Session(bind=connection)

    yield session

    session.close()
    transaction.rollback()
    connection.close()


@pytest.fixture
def sample_user(db_session: Session) -> User:
    """Create a sample user in the test database."""
    user = User(
        name="Test User",
        email="test@example.com",
        is_active=True,
    )
    db_session.add(user)
    db_session.flush()
    return user


@pytest.fixture
def auth_headers(sample_user: User) -> dict[str, str]:
    """Generate authentication headers for API tests."""
    token = sample_user.generate_access_token(expires_in=3600)
    return {"Authorization": f"Bearer {token}"}
```

### Autouse Fixtures

Autouse fixtures run automatically for every test in their scope without being explicitly requested.

```python
@pytest.fixture(autouse=True)
def reset_caches() -> Generator[None, None, None]:
    """Clear all caches before each test to ensure isolation."""
    yield
    from myapp.cache import cache_registry
    cache_registry.clear_all()


@pytest.fixture(autouse=True, scope="session")
def configure_logging() -> None:
    """Set up logging for the test session."""
    import logging
    logging.basicConfig(level=logging.WARNING)
```

### Fixture Parametrization

```python
@pytest.fixture(params=["sqlite", "postgresql"])
def database_url(request: pytest.FixtureRequest) -> str:
    """Run tests against multiple database backends."""
    urls = {
        "sqlite": "sqlite:///test.db",
        "postgresql": "postgresql://test:test@localhost:5432/testdb",
    }
    return urls[request.param]
```

---

## Factory Fixtures and factory_boy

Factory fixtures create test data with sensible defaults while allowing customization. The `factory_boy` library integrates with ORMs like SQLAlchemy and Django.

```python
# tests/factories.py
from __future__ import annotations

from datetime import datetime, timezone
from decimal import Decimal
from typing import Any

import factory
from factory.alchemy import SQLAlchemyModelFactory

from myapp.models import User, Order, OrderItem, OrderStatus, Address


class UserFactory(SQLAlchemyModelFactory):
    """Factory for creating User model instances."""

    class Meta:
        model = User
        sqlalchemy_session = None  # Set dynamically via conftest
        sqlalchemy_session_persistence = "commit"

    name = factory.Faker("name")
    email = factory.LazyAttribute(lambda obj: f"{obj.name.lower().replace(' ', '.')}@example.com")
    is_active = True
    created_at = factory.LazyFunction(lambda: datetime.now(timezone.utc))

    class Params:
        admin = factory.Trait(
            is_admin=True,
            role="admin",
        )
        inactive = factory.Trait(
            is_active=False,
            deactivated_at=factory.LazyFunction(lambda: datetime.now(timezone.utc)),
        )


class AddressFactory(SQLAlchemyModelFactory):
    """Factory for creating Address instances."""

    class Meta:
        model = Address
        sqlalchemy_session = None
        sqlalchemy_session_persistence = "commit"

    street = factory.Faker("street_address")
    city = factory.Faker("city")
    state = factory.Faker("state_abbr")
    zip_code = factory.Faker("zipcode")
    country = "US"
    user = factory.SubFactory(UserFactory)


class OrderFactory(SQLAlchemyModelFactory):
    """Factory for creating Order instances with related items."""

    class Meta:
        model = Order
        sqlalchemy_session = None
        sqlalchemy_session_persistence = "commit"

    user = factory.SubFactory(UserFactory)
    status = OrderStatus.PENDING
    shipping_address = factory.SubFactory(AddressFactory)
    created_at = factory.LazyFunction(lambda: datetime.now(timezone.utc))
    total = Decimal("0.00")

    @factory.post_generation
    def items(self, create: bool, extracted: list[dict[str, Any]] | None, **kwargs: Any) -> None:
        if not create:
            return
        if extracted:
            for item_data in extracted:
                OrderItemFactory(order=self, **item_data)
            self.total = sum(item.subtotal for item in self.items)


class OrderItemFactory(SQLAlchemyModelFactory):
    """Factory for creating OrderItem instances."""

    class Meta:
        model = OrderItem
        sqlalchemy_session = None
        sqlalchemy_session_persistence = "commit"

    order = factory.SubFactory(OrderFactory)
    product_name = factory.Faker("word")
    quantity = factory.Faker("random_int", min=1, max=10)
    unit_price = factory.Faker("pydecimal", left_digits=3, right_digits=2, positive=True)
    subtotal = factory.LazyAttribute(lambda obj: obj.quantity * obj.unit_price)
```

### Using Factories in conftest.py

```python
# tests/conftest.py
from __future__ import annotations

import pytest

from tests.factories import UserFactory, OrderFactory, AddressFactory, OrderItemFactory


@pytest.fixture(autouse=True)
def _set_factory_sessions(db_session):
    """Bind all factories to the current test session."""
    for factory_cls in [UserFactory, OrderFactory, AddressFactory, OrderItemFactory]:
        factory_cls._meta.sqlalchemy_session = db_session
    yield


@pytest.fixture
def user_factory():
    """Expose UserFactory for custom creation in tests."""
    return UserFactory


@pytest.fixture
def order_factory():
    """Expose OrderFactory for custom creation in tests."""
    return OrderFactory
```

### Using Factories in Tests

```python
# tests/unit/test_order_service.py
from __future__ import annotations

from decimal import Decimal

from myapp.services import OrderService
from tests.factories import UserFactory, OrderFactory


class TestOrderService:
    def test_calculate_order_total(self, db_session) -> None:
        user = UserFactory()
        order = OrderFactory(
            user=user,
            items=[
                {"product_name": "Widget", "quantity": 2, "unit_price": Decimal("9.99")},
                {"product_name": "Gadget", "quantity": 1, "unit_price": Decimal("24.99")},
            ],
        )
        total = OrderService.calculate_total(order)
        assert total == pytest.approx(Decimal("44.97"))

    def test_bulk_order_creation(self, db_session) -> None:
        user = UserFactory()
        orders = OrderFactory.create_batch(5, user=user)
        assert len(orders) == 5
        assert all(o.user_id == user.id for o in orders)

    def test_admin_can_view_all_orders(self, db_session) -> None:
        admin = UserFactory(admin=True)
        regular = UserFactory()
        OrderFactory.create_batch(3, user=regular)

        orders = OrderService.list_orders(requesting_user=admin)
        assert len(orders) == 3
```

---

## conftest.py Organization

`conftest.py` files provide fixtures to all tests in their directory and subdirectories. Organize them hierarchically:

- `tests/conftest.py` — shared fixtures (db, app, config)
- `tests/unit/conftest.py` — unit-test-specific fixtures
- `tests/integration/conftest.py` — integration-test-specific fixtures (real DB connections, external services)

### Key Rules

- Fixtures in `conftest.py` are automatically available without imports
- A fixture in a child `conftest.py` overrides one with the same name in a parent
- Keep `conftest.py` files focused; avoid putting test functions in them
- Use `conftest.py` for fixture sharing, not for utility functions (put those in `tests/helpers/`)

---

## unittest.mock -- Mock, MagicMock, patch

The `unittest.mock` module provides tools for replacing parts of your system under test with mock objects.

### Mock vs MagicMock

- `Mock`: base mock object with configurable return values and side effects
- `MagicMock`: extends `Mock` with default implementations of magic methods (`__len__`, `__iter__`, etc.)
- Use `MagicMock` unless you specifically need to test magic method behavior

### patch Decorator and Context Manager

```python
# tests/unit/test_services.py
from __future__ import annotations

from datetime import datetime, timezone
from decimal import Decimal
from typing import Any
from unittest.mock import AsyncMock, MagicMock, Mock, call, patch

import pytest

from myapp.services import NotificationService, PaymentService, UserService


class TestUserService:
    """Demonstrate mock patterns for service-layer testing."""

    @patch("myapp.services.user_service.email_client")
    def test_send_welcome_email(self, mock_email_client: MagicMock) -> None:
        """Patch an imported module-level object."""
        mock_email_client.send.return_value = {"message_id": "abc123"}

        service = UserService()
        result = service.register_user(name="Alice", email="alice@example.com")

        mock_email_client.send.assert_called_once_with(
            to="alice@example.com",
            subject="Welcome to MyApp",
            template="welcome",
            context={"name": "Alice"},
        )
        assert result.email_sent is True

    @patch("myapp.services.user_service.datetime")
    def test_user_created_with_current_timestamp(self, mock_datetime: MagicMock) -> None:
        """Mock datetime to control time-dependent logic."""
        fixed_now = datetime(2026, 1, 15, 12, 0, 0, tzinfo=timezone.utc)
        mock_datetime.now.return_value = fixed_now
        mock_datetime.side_effect = lambda *a, **kw: datetime(*a, **kw)

        service = UserService()
        user = service.register_user(name="Bob", email="bob@example.com")

        assert user.created_at == fixed_now


class TestPaymentService:
    """Demonstrate side_effect for complex mock behavior."""

    def test_retry_on_transient_failure(self) -> None:
        """Use side_effect with a list to simulate retry scenarios."""
        mock_gateway = MagicMock()
        mock_gateway.charge.side_effect = [
            ConnectionError("timeout"),
            ConnectionError("timeout"),
            {"transaction_id": "txn_789", "status": "success"},
        ]

        service = PaymentService(gateway=mock_gateway)
        result = service.charge_with_retry(
            amount=Decimal("49.99"),
            card_token="tok_abc",
            max_retries=3,
        )

        assert result["status"] == "success"
        assert mock_gateway.charge.call_count == 3

    def test_side_effect_as_function(self) -> None:
        """Use side_effect as a callable for dynamic responses."""
        def dynamic_charge(amount: Decimal, **kwargs: Any) -> dict[str, Any]:
            if amount > Decimal("1000"):
                raise PaymentService.FraudDetectedError("Amount exceeds threshold")
            return {"transaction_id": "txn_001", "status": "success"}

        mock_gateway = MagicMock()
        mock_gateway.charge.side_effect = dynamic_charge

        service = PaymentService(gateway=mock_gateway)

        result = service.process_payment(amount=Decimal("50.00"))
        assert result["status"] == "success"

        with pytest.raises(PaymentService.FraudDetectedError):
            service.process_payment(amount=Decimal("5000.00"))


class TestNotificationService:
    """Demonstrate spec-based mocking and call assertions."""

    def test_spec_mock_prevents_typos(self) -> None:
        """Use spec to catch attribute errors on mocks."""
        mock_notifier = Mock(spec=NotificationService)

        # This would raise AttributeError because send_sms is not in the spec:
        # mock_notifier.send_sms("123", "hello")

        mock_notifier.send_push_notification(user_id="u123", message="Hello")
        mock_notifier.send_push_notification.assert_called_once()

    def test_assert_call_order(self) -> None:
        """Verify that methods are called in a specific order."""
        mock_notifier = MagicMock()

        mock_notifier.connect()
        mock_notifier.authenticate(token="abc")
        mock_notifier.send(message="hello")
        mock_notifier.disconnect()

        expected_calls = [
            call.connect(),
            call.authenticate(token="abc"),
            call.send(message="hello"),
            call.disconnect(),
        ]
        mock_notifier.assert_has_calls(expected_calls, any_order=False)

    @patch.multiple(
        "myapp.services.notification_service",
        sms_client=MagicMock(),
        push_client=MagicMock(),
        email_client=MagicMock(),
    )
    def test_patch_multiple_dependencies(self, **mocks: MagicMock) -> None:
        """Patch multiple module-level objects at once."""
        service = NotificationService()
        service.broadcast(user_id="u123", message="System update")
        # Each client's send method should be called
```

### patch.object for Instance Methods

```python
class TestPatchObject:
    def test_patch_instance_method(self) -> None:
        """Patch a method on a specific class."""
        with patch.object(UserService, "validate_email", return_value=True) as mock_validate:
            service = UserService()
            service.register_user(name="Alice", email="test@test.com")
            mock_validate.assert_called_once_with("test@test.com")
```

---

## Testing Flask Applications

Flask provides built-in test utilities through `app.test_client()` and application/request contexts.

```python
# tests/conftest.py
from __future__ import annotations

from collections.abc import Generator
from typing import Any

import pytest
from flask import Flask
from flask.testing import FlaskClient

from myapp.app import create_app
from myapp.database import db as _db
from myapp.models import User


@pytest.fixture(scope="session")
def app() -> Generator[Flask, None, None]:
    """Create the Flask application for testing."""
    app = create_app(config_name="testing")
    with app.app_context():
        _db.create_all()
        yield app
        _db.drop_all()


@pytest.fixture(scope="function")
def db(app: Flask) -> Generator[Any, None, None]:
    """Provide a clean database for each test via transactions."""
    connection = _db.engine.connect()
    transaction = connection.begin()
    options = {"bind": connection, "binds": {}}
    session = _db.create_scoped_session(options=options)
    _db.session = session

    yield _db

    transaction.rollback()
    connection.close()
    session.remove()


@pytest.fixture
def client(app: Flask) -> FlaskClient:
    """Create a Flask test client."""
    return app.test_client()


@pytest.fixture
def auth_client(client: FlaskClient, db: Any) -> FlaskClient:
    """Create an authenticated test client."""
    user = User(name="Test Admin", email="admin@test.com", is_admin=True)
    db.session.add(user)
    db.session.commit()

    token = user.generate_access_token()
    client.environ_base["HTTP_AUTHORIZATION"] = f"Bearer {token}"
    return client
```

### Testing API Endpoints

```python
# tests/integration/test_api.py
from __future__ import annotations

import json
from http import HTTPStatus
from typing import Any

import pytest
from flask.testing import FlaskClient


class TestUserAPI:
    """Integration tests for the User API endpoints."""

    def test_create_user(self, auth_client: FlaskClient, db: Any) -> None:
        response = auth_client.post(
            "/api/v1/users",
            json={"name": "Alice", "email": "alice@example.com", "role": "editor"},
        )

        assert response.status_code == HTTPStatus.CREATED
        data = response.get_json()
        assert data["name"] == "Alice"
        assert data["email"] == "alice@example.com"
        assert "id" in data

    def test_create_user_duplicate_email(self, auth_client: FlaskClient, db: Any) -> None:
        """Duplicate emails should return 409 Conflict."""
        payload = {"name": "Alice", "email": "alice@example.com", "role": "editor"}
        auth_client.post("/api/v1/users", json=payload)
        response = auth_client.post("/api/v1/users", json=payload)

        assert response.status_code == HTTPStatus.CONFLICT
        assert "already registered" in response.get_json()["error"]

    def test_create_user_validation_error(self, auth_client: FlaskClient) -> None:
        """Invalid payloads should return 422."""
        response = auth_client.post(
            "/api/v1/users",
            json={"name": "", "email": "not-an-email"},
        )

        assert response.status_code == HTTPStatus.UNPROCESSABLE_ENTITY
        errors = response.get_json()["errors"]
        assert "name" in errors
        assert "email" in errors

    def test_list_users_pagination(self, auth_client: FlaskClient, db: Any) -> None:
        """Verify pagination parameters work correctly."""
        for i in range(25):
            auth_client.post(
                "/api/v1/users",
                json={"name": f"User {i}", "email": f"user{i}@example.com", "role": "viewer"},
            )

        response = auth_client.get("/api/v1/users?page=2&per_page=10")
        assert response.status_code == HTTPStatus.OK
        data = response.get_json()
        assert len(data["items"]) == 10
        assert data["total"] == 25
        assert data["page"] == 2

    def test_unauthenticated_request(self, client: FlaskClient) -> None:
        """Requests without auth should return 401."""
        response = client.get("/api/v1/users")
        assert response.status_code == HTTPStatus.UNAUTHORIZED


class TestFlaskAppContext:
    """Tests that require Flask app or request context."""

    def test_app_context_access(self, app: Any) -> None:
        """Access current_app within app context."""
        from flask import current_app

        with app.app_context():
            assert current_app.config["TESTING"] is True

    def test_request_context(self, app: Any) -> None:
        """Access request object within request context."""
        from flask import request

        with app.test_request_context("/api/v1/users?page=2", method="GET"):
            assert request.path == "/api/v1/users"
            assert request.args["page"] == "2"
```

---

## Integration Testing with Databases

Integration tests verify that your application works correctly with a real database. Use transactional rollback to keep tests isolated and fast.

### SQLAlchemy Transactional Test Pattern

The `db_session` fixture shown in the Fixtures section above wraps each test in a transaction and rolls back after the test completes. This ensures:

- Each test starts with a clean state
- No test data leaks between tests
- Tests are fast because no data is actually written to disk

### Testing with a Dedicated Test Database

```python
# tests/conftest.py (additional fixtures for full DB integration)
from __future__ import annotations

import os
from collections.abc import Generator
from typing import Any

import pytest
from sqlalchemy import create_engine, event, text
from sqlalchemy.orm import Session, sessionmaker

from myapp.models import Base


@pytest.fixture(scope="session")
def test_database_url() -> str:
    """Read the test database URL from environment or use default."""
    return os.environ.get(
        "TEST_DATABASE_URL",
        "postgresql://test:test@localhost:5432/myapp_test",
    )


@pytest.fixture(scope="session")
def engine(test_database_url: str) -> Generator[Any, None, None]:
    """Create a SQLAlchemy engine for the test database."""
    eng = create_engine(test_database_url, echo=False)
    Base.metadata.create_all(eng)
    yield eng
    Base.metadata.drop_all(eng)
    eng.dispose()


@pytest.fixture(scope="function")
def db_session(engine: Any) -> Generator[Session, None, None]:
    """Provide a transactional session with SAVEPOINT support."""
    connection = engine.connect()
    transaction = connection.begin()
    session = Session(bind=connection)

    # Begin a nested transaction (SAVEPOINT)
    nested = connection.begin_nested()

    # Restart the SAVEPOINT when the application code calls session.commit()
    @event.listens_for(session, "after_transaction_end")
    def restart_savepoint(session: Session, transaction_inner: Any) -> None:
        nonlocal nested
        if transaction_inner.nested and not transaction_inner._parent.nested:
            nested = connection.begin_nested()

    yield session

    session.close()
    transaction.rollback()
    connection.close()
```

### Testing Migrations with Alembic

```python
# tests/integration/test_migrations.py
from __future__ import annotations

from alembic import command
from alembic.config import Config


def test_migrations_up_and_down() -> None:
    """Verify all migrations can be applied and rolled back."""
    alembic_cfg = Config("alembic.ini")
    alembic_cfg.set_main_option("sqlalchemy.url", "sqlite:///test_migration.db")

    # Apply all migrations
    command.upgrade(alembic_cfg, "head")
    # Roll back all migrations
    command.downgrade(alembic_cfg, "base")
    # Apply again to verify idempotency
    command.upgrade(alembic_cfg, "head")
```

---

## Testing boto3/AWS with moto

The `moto` library provides mock implementations of AWS services, letting you test boto3 code without connecting to real AWS infrastructure.

```python
# tests/unit/test_aws_services.py
from __future__ import annotations

import json
from typing import Any

import boto3
import pytest
from moto import mock_aws

from myapp.services.storage import S3StorageService
from myapp.services.queue import SQSQueueService
from myapp.services.secrets import SecretsService


@pytest.fixture
def aws_credentials() -> None:
    """Set dummy AWS credentials for moto."""
    import os
    os.environ["AWS_ACCESS_KEY_ID"] = "testing"
    os.environ["AWS_SECRET_ACCESS_KEY"] = "testing"
    os.environ["AWS_SECURITY_TOKEN"] = "testing"
    os.environ["AWS_SESSION_TOKEN"] = "testing"
    os.environ["AWS_DEFAULT_REGION"] = "us-east-1"


@pytest.fixture
def s3_bucket(aws_credentials: None) -> str:
    """Create a mock S3 bucket."""
    with mock_aws():
        s3 = boto3.client("s3", region_name="us-east-1")
        bucket_name = "test-bucket"
        s3.create_bucket(Bucket=bucket_name)
        yield bucket_name


class TestS3StorageService:
    """Test S3 operations using moto mock."""

    @mock_aws
    def test_upload_file(self, aws_credentials: None) -> None:
        s3 = boto3.client("s3", region_name="us-east-1")
        s3.create_bucket(Bucket="my-bucket")

        service = S3StorageService(bucket_name="my-bucket")
        service.upload(key="reports/2026/q1.pdf", data=b"PDF content here")

        response = s3.get_object(Bucket="my-bucket", Key="reports/2026/q1.pdf")
        assert response["Body"].read() == b"PDF content here"

    @mock_aws
    def test_list_objects_with_prefix(self, aws_credentials: None) -> None:
        s3 = boto3.client("s3", region_name="us-east-1")
        s3.create_bucket(Bucket="my-bucket")
        for i in range(5):
            s3.put_object(Bucket="my-bucket", Key=f"logs/2026/01/{i}.log", Body=b"log data")
        s3.put_object(Bucket="my-bucket", Key="logs/2025/12/old.log", Body=b"old log")

        service = S3StorageService(bucket_name="my-bucket")
        keys = service.list_keys(prefix="logs/2026/01/")

        assert len(keys) == 5

    @mock_aws
    def test_delete_nonexistent_key_raises(self, aws_credentials: None) -> None:
        s3 = boto3.client("s3", region_name="us-east-1")
        s3.create_bucket(Bucket="my-bucket")

        service = S3StorageService(bucket_name="my-bucket")
        with pytest.raises(service.ObjectNotFoundError):
            service.delete(key="nonexistent/file.txt")


class TestSQSQueueService:
    """Test SQS operations using moto mock."""

    @mock_aws
    def test_send_and_receive_message(self, aws_credentials: None) -> None:
        sqs = boto3.client("sqs", region_name="us-east-1")
        queue_url = sqs.create_queue(QueueName="test-queue")["QueueUrl"]

        service = SQSQueueService(queue_url=queue_url)
        service.send_message(body=json.dumps({"event": "user.created", "user_id": "u123"}))

        messages = service.receive_messages(max_count=1)
        assert len(messages) == 1
        payload = json.loads(messages[0]["Body"])
        assert payload["event"] == "user.created"


class TestSecretsManager:
    """Test Secrets Manager operations using moto."""

    @mock_aws
    def test_get_secret_value(self, aws_credentials: None) -> None:
        sm = boto3.client("secretsmanager", region_name="us-east-1")
        sm.create_secret(
            Name="myapp/database_url",
            SecretString="postgresql://prod:secret@db.example.com/myapp",
        )

        service = SecretsService()
        secret = service.get_secret("myapp/database_url")
        assert "db.example.com" in secret
```

---

## Testing Subprocess Calls

When your application shells out to external commands, mock `subprocess.run` or `subprocess.Popen` to avoid executing real commands in tests.

```python
# tests/unit/test_subprocess.py
from __future__ import annotations

import subprocess
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from myapp.services.git import GitService
from myapp.services.ffmpeg import FFmpegService


class TestGitService:
    """Test code that wraps git commands."""

    @patch("myapp.services.git.subprocess.run")
    def test_get_current_branch(self, mock_run: MagicMock) -> None:
        mock_run.return_value = subprocess.CompletedProcess(
            args=["git", "branch", "--show-current"],
            returncode=0,
            stdout="feature/user-auth\n",
            stderr="",
        )

        service = GitService(repo_path=Path("/tmp/repo"))
        branch = service.get_current_branch()

        assert branch == "feature/user-auth"
        mock_run.assert_called_once_with(
            ["git", "branch", "--show-current"],
            cwd=Path("/tmp/repo"),
            capture_output=True,
            text=True,
            check=True,
            timeout=30,
        )

    @patch("myapp.services.git.subprocess.run")
    def test_git_command_failure_raises(self, mock_run: MagicMock) -> None:
        mock_run.side_effect = subprocess.CalledProcessError(
            returncode=128,
            cmd=["git", "push"],
            stderr="fatal: remote rejected",
        )

        service = GitService(repo_path=Path("/tmp/repo"))
        with pytest.raises(GitService.CommandError, match="remote rejected"):
            service.push(remote="origin", branch="main")


class TestFFmpegService:
    """Test code that wraps ffmpeg commands."""

    @patch("myapp.services.ffmpeg.subprocess.run")
    def test_convert_video(self, mock_run: MagicMock) -> None:
        mock_run.return_value = subprocess.CompletedProcess(
            args=["ffmpeg", "-i", "input.mp4", "output.webm"],
            returncode=0,
            stdout="",
            stderr="Conversion complete",
        )

        service = FFmpegService()
        result = service.convert(
            input_path=Path("input.mp4"),
            output_path=Path("output.webm"),
            codec="vp9",
        )

        assert result.success is True
        cmd_args = mock_run.call_args[0][0]
        assert "ffmpeg" in cmd_args
        assert "-codec:v" in cmd_args or "-c:v" in cmd_args

    @patch("myapp.services.ffmpeg.subprocess.run")
    def test_timeout_handling(self, mock_run: MagicMock) -> None:
        mock_run.side_effect = subprocess.TimeoutExpired(
            cmd=["ffmpeg"], timeout=300
        )

        service = FFmpegService()
        with pytest.raises(FFmpegService.ConversionTimeoutError):
            service.convert(
                input_path=Path("huge_video.mp4"),
                output_path=Path("output.webm"),
                codec="vp9",
                timeout=300,
            )
```

---

## Testing Async Code with pytest-asyncio

`pytest-asyncio` allows you to write test functions as coroutines. Use it for testing `asyncio`-based code such as async web frameworks, async database drivers, and async HTTP clients.

### Configuration

```toml
# pyproject.toml
[tool.pytest.ini_options]
asyncio_mode = "auto"  # or "strict" to require explicit marks
```

### Async Test Examples

```python
# tests/unit/test_async_services.py
from __future__ import annotations

from collections.abc import AsyncGenerator
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
import pytest_asyncio

from myapp.services.async_http import AsyncHTTPClient
from myapp.services.async_cache import AsyncRedisCache
from myapp.services.async_worker import TaskWorker


@pytest_asyncio.fixture
async def async_cache() -> AsyncGenerator[AsyncRedisCache, None]:
    """Provide an async Redis cache connection for tests."""
    cache = AsyncRedisCache(url="redis://localhost:6379/15")
    await cache.connect()
    yield cache
    await cache.flush_db()
    await cache.disconnect()


class TestAsyncHTTPClient:
    """Test async HTTP client operations."""

    async def test_fetch_json(self) -> None:
        mock_response = AsyncMock()
        mock_response.status = 200
        mock_response.json = AsyncMock(return_value={"data": [1, 2, 3]})

        with patch.object(AsyncHTTPClient, "_request", return_value=mock_response):
            client = AsyncHTTPClient(base_url="https://api.example.com")
            result = await client.fetch_json("/data")

            assert result == {"data": [1, 2, 3]}

    async def test_retry_on_server_error(self) -> None:
        """Verify async retry logic on 5xx errors."""
        responses = [
            AsyncMock(status=503),
            AsyncMock(status=503),
            AsyncMock(status=200, json=AsyncMock(return_value={"ok": True})),
        ]

        with patch.object(AsyncHTTPClient, "_request", side_effect=responses):
            client = AsyncHTTPClient(base_url="https://api.example.com")
            result = await client.fetch_json("/health", retries=3)

            assert result == {"ok": True}


class TestAsyncRedisCache:
    """Test async cache operations (requires running Redis or use fakeredis)."""

    async def test_set_and_get(self, async_cache: AsyncRedisCache) -> None:
        await async_cache.set("user:123", '{"name": "Alice"}', ttl=60)
        value = await async_cache.get("user:123")
        assert value == '{"name": "Alice"}'

    async def test_get_missing_key_returns_none(self, async_cache: AsyncRedisCache) -> None:
        value = await async_cache.get("nonexistent:key")
        assert value is None

    async def test_delete(self, async_cache: AsyncRedisCache) -> None:
        await async_cache.set("temp:key", "value", ttl=10)
        deleted = await async_cache.delete("temp:key")
        assert deleted is True
        assert await async_cache.get("temp:key") is None


class TestTaskWorker:
    """Test async background task processing."""

    async def test_process_task_batch(self) -> None:
        mock_processor = AsyncMock(return_value={"status": "completed"})
        worker = TaskWorker(processor=mock_processor, concurrency=5)

        tasks = [{"id": i, "type": "email"} for i in range(10)]
        results = await worker.process_batch(tasks)

        assert len(results) == 10
        assert all(r["status"] == "completed" for r in results)
        assert mock_processor.call_count == 10
```

---

## Performance Testing with pytest-benchmark

`pytest-benchmark` provides precise benchmarking of Python code. It handles warmup, iterations, statistical analysis, and comparison between runs.

```python
# tests/performance/test_benchmarks.py
from __future__ import annotations

import json
from typing import Any

import pytest
from pytest_benchmark.fixture import BenchmarkFixture

from myapp.serializers import serialize_user_list, serialize_order
from myapp.services import SearchService


class TestSerializationPerformance:
    """Benchmark serialization operations."""

    def test_serialize_user_list(self, benchmark: BenchmarkFixture) -> None:
        """Benchmark user list serialization."""
        users = [
            {"id": i, "name": f"User {i}", "email": f"user{i}@example.com"}
            for i in range(1000)
        ]

        result = benchmark(serialize_user_list, users)

        assert len(result) == 1000

    def test_serialize_order_with_items(self, benchmark: BenchmarkFixture) -> None:
        """Benchmark complex nested serialization."""
        order = {
            "id": 1,
            "items": [{"product": f"Item {i}", "qty": i, "price": 9.99} for i in range(50)],
            "total": 499.50,
        }

        result = benchmark(serialize_order, order)
        assert result["total"] == 499.50

    @pytest.mark.parametrize("size", [100, 1000, 10_000])
    def test_json_serialization_scaling(
        self, benchmark: BenchmarkFixture, size: int
    ) -> None:
        """Benchmark JSON serialization at different data sizes."""
        data = [{"key": f"value_{i}", "number": i} for i in range(size)]

        result = benchmark(json.dumps, data)

        assert isinstance(result, str)


class TestSearchPerformance:
    """Benchmark search operations."""

    def test_full_text_search(self, benchmark: BenchmarkFixture, db_session: Any) -> None:
        """Benchmark full-text search performance."""
        service = SearchService(session=db_session)

        result = benchmark.pedantic(
            service.search,
            args=("python testing",),
            kwargs={"limit": 50},
            iterations=10,
            rounds=5,
            warmup_rounds=2,
        )

        assert isinstance(result, list)
```

### Running Benchmarks

```
pytest tests/performance/ --benchmark-only
pytest tests/performance/ --benchmark-compare
pytest tests/performance/ --benchmark-save=baseline
pytest tests/performance/ --benchmark-compare=0001_baseline --benchmark-group-by=func
```

---

## pytest-cov for Coverage Reports

`pytest-cov` integrates coverage.py with pytest to measure code coverage during test execution.

### Configuration

```toml
# pyproject.toml
[tool.coverage.run]
source = ["src/myapp"]
branch = true
omit = [
    "*/migrations/*",
    "*/tests/*",
    "*/__main__.py",
]

[tool.coverage.report]
show_missing = true
skip_covered = false
fail_under = 85
exclude_lines = [
    "pragma: no cover",
    "def __repr__",
    "if TYPE_CHECKING:",
    "if __name__ == .__main__.:",
    "raise NotImplementedError",
    "pass",
    "@abstractmethod",
]

[tool.coverage.html]
directory = "htmlcov"
```

### Running Coverage

```
# Basic coverage report
pytest --cov=src/myapp --cov-report=term-missing

# HTML report for detailed line-by-line analysis
pytest --cov=src/myapp --cov-report=html --cov-report=term

# XML report for CI integration (Codecov, Coveralls)
pytest --cov=src/myapp --cov-report=xml:coverage.xml

# Fail if coverage drops below threshold
pytest --cov=src/myapp --cov-fail-under=85

# Combine with parallel execution
pytest -n auto --cov=src/myapp --cov-report=term-missing
```

### Coverage Pragmas

Use `# pragma: no cover` sparingly for code that genuinely cannot be tested:

```python
if TYPE_CHECKING:  # pragma: no cover
    from myapp.models import User

def unreachable_defensive_code() -> None:
    raise RuntimeError("This should never happen")  # pragma: no cover
```

---

## Test Organization and Project Structure

### Separation of Concerns

Organize tests by type and keep them close to what they test:

```
tests/
├── conftest.py              # Shared fixtures (db, app, auth)
├── factories.py             # factory_boy factories
├── helpers/
│   ├── __init__.py
│   ├── assertions.py        # Custom assertion helpers
│   └── builders.py          # Test data builders
├── unit/
│   ├── conftest.py          # Unit-specific fixtures (mocks)
│   ├── test_models.py
│   ├── test_services.py
│   ├── test_serializers.py
│   └── test_validators.py
├── integration/
│   ├── conftest.py          # Integration fixtures (real DB, real HTTP)
│   ├── test_api_users.py
│   ├── test_api_orders.py
│   └── test_repositories.py
├── e2e/
│   ├── conftest.py
│   └── test_user_workflows.py
└── performance/
    ├── conftest.py
    └── test_benchmarks.py
```

### Makefile Targets

```makefile
.PHONY: test test-unit test-integration test-cov test-fast

test:
	pytest

test-unit:
	pytest tests/unit/ -m "not slow"

test-integration:
	pytest tests/integration/ -m "integration"

test-cov:
	pytest --cov=src/myapp --cov-report=html --cov-report=term-missing

test-fast:
	pytest -x -q --tb=short -n auto

test-watch:
	ptw -- -x -q --tb=short
```

---

## CI Integration and Parallel Test Execution

### pytest-xdist for Parallel Execution

`pytest-xdist` runs tests in parallel across multiple CPUs. It is essential for large test suites.

```
# Use all available CPUs
pytest -n auto

# Use a specific number of workers
pytest -n 4

# Distribute by file (default) or by test
pytest -n auto --dist loadfile
pytest -n auto --dist loadscope
```

### Considerations for Parallel Tests

- Tests must be fully isolated (no shared mutable state)
- Use unique database names per worker: `myapp_test_gw0`, `myapp_test_gw1`, etc.
- File-based resources need unique paths per worker
- Use `tmp_path` fixture for temporary files (pytest provides unique paths per test)

### Worker-Specific Database Fixtures

```python
# tests/conftest.py
from __future__ import annotations

import os
from typing import Any

import pytest


@pytest.fixture(scope="session")
def worker_database_url(worker_id: str) -> str:
    """Generate a unique database URL per xdist worker."""
    base_url = os.environ.get(
        "TEST_DATABASE_URL",
        "postgresql://test:test@localhost:5432",
    )
    if worker_id == "master":
        return f"{base_url}/myapp_test"
    return f"{base_url}/myapp_test_{worker_id}"
```

### GitHub Actions CI Configuration

```yaml
# .github/workflows/test.yml
name: Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: test
          POSTGRES_PASSWORD: test
          POSTGRES_DB: myapp_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      redis:
        image: redis:7
        ports:
          - 6379:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - uses: actions/checkout@v4

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: "3.12"
          cache: "pip"

      - name: Install dependencies
        run: |
          python -m pip install --upgrade pip
          pip install -e ".[test]"

      - name: Create parallel test databases
        env:
          PGHOST: localhost
          PGPORT: 5432
          PGUSER: test
          PGPASSWORD: test
        run: |
          for i in $(seq 0 3); do
            createdb "myapp_test_gw${i}" || true
          done

      - name: Run tests with coverage
        env:
          TEST_DATABASE_URL: postgresql://test:test@localhost:5432
          REDIS_URL: redis://localhost:6379/15
        run: |
          pytest -n 4 --cov=src/myapp --cov-report=xml --cov-report=term-missing

      - name: Upload coverage to Codecov
        uses: codecov/codecov-action@v4
        with:
          file: coverage.xml
          fail_ci_if_error: true
          token: ${{ secrets.CODECOV_TOKEN }}

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Python
        uses: actions/setup-python@v5
        with:
          python-version: "3.12"

      - name: Install dependencies
        run: pip install ruff mypy

      - name: Lint
        run: ruff check src/ tests/

      - name: Type check
        run: mypy src/
```

### GitLab CI Configuration

```yaml
# .gitlab-ci.yml
stages:
  - test

test:
  stage: test
  image: python:3.12-slim
  services:
    - postgres:16
    - redis:7
  variables:
    POSTGRES_DB: myapp_test
    POSTGRES_USER: test
    POSTGRES_PASSWORD: test
    TEST_DATABASE_URL: postgresql://test:test@postgres:5432/myapp_test
    REDIS_URL: redis://redis:6379/15
  before_script:
    - pip install -e ".[test]"
  script:
    - pytest -n auto --cov=src/myapp --cov-report=xml --cov-report=term-missing --junitxml=report.xml
  artifacts:
    reports:
      junit: report.xml
      coverage_report:
        coverage_format: cobertura
        path: coverage.xml
  coverage: '/(?i)total.*? (100(?:\.0+)?\%|[1-9]?\d(?:\.\d+)?\%)$/'
```

---

## Best Practices

### 1. Follow the Arrange-Act-Assert (AAA) Pattern

Every test should have three clearly separated phases:

```python
def test_apply_discount(self, db_session: Any) -> None:
    # Arrange
    order = OrderFactory(total=Decimal("100.00"))
    coupon = CouponFactory(discount_percent=15)

    # Act
    result = OrderService.apply_coupon(order=order, coupon=coupon)

    # Assert
    assert result.total == Decimal("85.00")
    assert result.applied_coupon_id == coupon.id
```

### 2. Test Behavior, Not Implementation

- Test the public interface, not private methods
- Assert on outcomes and side effects, not internal state
- If refactoring breaks tests but not behavior, the tests are too coupled

### 3. Use Fixtures for Setup, Not Helper Functions Called in Test Bodies

Fixtures make dependencies explicit and enable automatic cleanup. Prefer `@pytest.fixture` over shared setup functions called manually in each test.

### 4. Keep Tests Independent and Idempotent

- Never rely on test execution order
- Each test must set up its own state
- Use transactional rollback for database tests
- Clean up external resources in fixture teardown

### 5. Use Parametrize for Data-Driven Tests

Instead of writing five nearly identical test functions, use `@pytest.mark.parametrize` with descriptive `ids`.

### 6. Mock at the Boundary

- Mock external HTTP calls, databases, file systems, and clocks
- Do not mock the class under test
- Prefer dependency injection over `patch` when possible

### 7. Write Descriptive Test Names

Use the pattern: `test_<unit>_<scenario>_<expected_outcome>`

- `test_register_user_with_existing_email_raises_conflict`
- `test_calculate_shipping_for_international_order_adds_customs_fee`
- `test_process_payment_on_gateway_timeout_retries_three_times`

### 8. Use Type Hints in Tests

Type hints in test code improve IDE support, catch errors early, and serve as documentation for fixture return types.

### 9. Prefer `tmp_path` Over `tempfile`

The `tmp_path` fixture provides a unique `pathlib.Path` per test with automatic cleanup:

```python
def test_export_report(self, tmp_path: Path) -> None:
    output_file = tmp_path / "report.csv"
    ReportService.export(output_path=output_file)
    assert output_file.exists()
    lines = output_file.read_text().splitlines()
    assert len(lines) > 1  # header + data
```

### 10. Set Strict Markers

Configure `--strict-markers` in `pyproject.toml` to catch typos in mark names at collection time rather than silently ignoring them.

### 11. Use `freezegun` or `time-machine` for Time-Dependent Tests

Rather than mocking `datetime` manually, use dedicated libraries:

```python
import time_machine

@time_machine.travel("2026-01-15 12:00:00", tick=False)
def test_token_expiration() -> None:
    token = generate_token(expires_in=3600)
    assert token.is_valid()
```

### 12. Fail Fast During Development

Use `-x` to stop on the first failure and `--tb=short` for concise tracebacks:

```
pytest -x --tb=short -q
```

---

## Anti-Patterns

### 1. Testing Implementation Details

```python
# BAD: Testing internal method calls and private attributes
def test_user_creation_bad(self) -> None:
    service = UserService()
    service.create_user("Alice", "alice@example.com")
    assert service._cache["alice@example.com"] is not None  # Fragile!
    assert service._validator._regex_compiled is True  # Too coupled!

# GOOD: Test the observable behavior
def test_user_creation_good(self, db_session: Any) -> None:
    service = UserService(session=db_session)
    user = service.create_user("Alice", "alice@example.com")
    assert user.name == "Alice"
    retrieved = service.get_user_by_email("alice@example.com")
    assert retrieved is not None
```

### 2. Mocking Everything

Over-mocking makes tests useless because they only verify that mocks were called correctly, not that the code works.

```python
# BAD: Mocking the thing you are trying to test
@patch("myapp.services.UserService.create_user")
def test_create_user(self, mock_create: MagicMock) -> None:
    mock_create.return_value = User(name="Alice")
    result = UserService().create_user("Alice", "alice@example.com")
    assert result.name == "Alice"  # This tests nothing!

# GOOD: Mock only external dependencies
def test_create_user(self, db_session: Any) -> None:
    with patch("myapp.services.email_client") as mock_email:
        service = UserService(session=db_session)
        user = service.create_user("Alice", "alice@example.com")
        assert user.name == "Alice"
        mock_email.send.assert_called_once()
```

### 3. Shared Mutable State Between Tests

```python
# BAD: Class-level mutable state shared across tests
class TestBad:
    results: list[str] = []  # Shared across all tests in this class!

    def test_one(self) -> None:
        self.results.append("one")
        assert len(self.results) == 1  # Might fail if test_two runs first

    def test_two(self) -> None:
        self.results.append("two")
        assert len(self.results) == 1  # Will fail if test_one runs first
```

### 4. Tests That Depend on Execution Order

Never assume tests run in a particular sequence. Each test must stand alone. Use `pytest-randomly` to detect order-dependent tests.

### 5. Catching Too-Broad Exceptions

```python
# BAD: This test passes even if a completely different exception is raised
def test_bad_exception_handling(self) -> None:
    with pytest.raises(Exception):
        do_something_risky()

# GOOD: Be specific about the exception type and message
def test_good_exception_handling(self) -> None:
    with pytest.raises(ValueError, match="Invalid email format"):
        validate_email("not-an-email")
```

### 6. Using `sleep()` in Tests

```python
# BAD: Flaky and slow
def test_background_job_completes(self) -> None:
    start_background_job()
    time.sleep(5)  # Hope it finishes in 5 seconds
    assert job_is_complete()

# GOOD: Poll with a timeout or mock the async behavior
def test_background_job_completes(self) -> None:
    with patch("myapp.tasks.process_job") as mock_job:
        future = start_background_job()
        future.result(timeout=10)  # Proper timeout handling
        mock_job.assert_called_once()
```

### 7. Not Cleaning Up External Resources

Always use fixture teardown (via `yield`) to clean up:

```python
# BAD: Leaked temp files
@pytest.fixture
def temp_file():
    f = open("/tmp/test_data.txt", "w")
    f.write("test")
    f.close()
    return Path("/tmp/test_data.txt")
    # File is never deleted!

# GOOD: Automatic cleanup with tmp_path
@pytest.fixture
def temp_file(tmp_path: Path) -> Path:
    f = tmp_path / "test_data.txt"
    f.write_text("test")
    return f  # Automatically cleaned up by pytest
```

### 8. Giant Test Functions

If a test function is longer than 30 lines, it is likely testing too many things. Split it into focused tests, each verifying one behavior.

### 9. Ignoring Warnings

Configure `filterwarnings = ["error"]` in pytest config to treat warnings as errors. This catches deprecation warnings before they become breaking changes.

### 10. Hard-Coding Paths and URLs

```python
# BAD
def test_config_loading(self) -> None:
    config = load_config("/home/developer/project/config.yaml")

# GOOD
def test_config_loading(self, tmp_path: Path) -> None:
    config_file = tmp_path / "config.yaml"
    config_file.write_text("database_url: sqlite:///test.db")
    config = load_config(config_file)
```

---

## Sources & References

- [pytest Documentation](https://docs.pytest.org/en/stable/) -- Official pytest documentation covering all features, plugins, and configuration options.
- [unittest.mock Documentation](https://docs.python.org/3/library/unittest.mock.html) -- Python standard library documentation for Mock, MagicMock, patch, and related mocking utilities.
- [moto - Mock AWS Services](https://docs.getmoto.org/en/latest/) -- Documentation for the moto library, covering mock implementations of S3, SQS, DynamoDB, Lambda, and 100+ other AWS services.
- [factory_boy Documentation](https://factoryboy.readthedocs.io/en/stable/) -- Guide to building test data factories with factory_boy, including SQLAlchemy and Django ORM integration.
- [Flask Testing Documentation](https://flask.palletsprojects.com/en/stable/testing/) -- Official Flask guide for testing applications with the test client, app contexts, and request contexts.
- [pytest-asyncio Documentation](https://pytest-asyncio.readthedocs.io/en/latest/) -- Plugin documentation for testing async/await Python code with pytest.
- [pytest-benchmark Documentation](https://pytest-benchmark.readthedocs.io/en/latest/) -- Guide to performance benchmarking with pytest, including statistical analysis and comparison features.
- [pytest-xdist Documentation](https://pytest-xdist.readthedocs.io/en/latest/) -- Documentation for parallel and distributed test execution with pytest.
- [Coverage.py Documentation](https://coverage.readthedocs.io/en/latest/) -- Comprehensive guide to measuring code coverage in Python, including branch coverage and configuration.
